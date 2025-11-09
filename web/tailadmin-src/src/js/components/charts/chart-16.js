import ApexCharts from "apexcharts";

// ===== chartSixteen
const chart16 = () => {
  const isDarkMode = document.documentElement.classList.contains("dark");
  const chartSixteenOptions = {
    series: [45, 65, 25, 25],
    colors: ["#9b8afb", "#fd853a", "#fdb022", "#32d583"],
    labels: ["Downloads", "Apps", "Documents", "Media"],
    chart: {
      fontFamily: "Outfit, sans-serif",
      type: "donut",
      width: 400,
    },
    stroke: {
      show: false,
      width: 4, // Creates a gap between the series
      colors: "transparent", // Gap color (use background color to make it seamless)
    },
    plotOptions: {
      pie: {
        donut: {
          lineCap: "smooth",
          size: "65%",
          background: "transparent",
          labels: {
            show: true,
            name: {
              show: true,
              offsetY: 0,
              color: isDarkMode ? "#ffffff" : "#1D2939",
              fontSize: "12px",
              fontWeight: "normal",
              text: "Total 135GB",
            },
            value: {
              show: true,
              offsetY: 10,
              color: isDarkMode ? "#1D2939" : "#667085",
              fontSize: "14px",
              formatter: () => "Used of 135 GB",
            },
            total: {
              show: true,
              label: "Total 135 GB",
              color: isDarkMode ? "#1D2939" : "#000000",
              fontSize: "24px",
              fontWeight: "bold",
            },
          },
        },
        expandOnClick: false,
      },
    },
    dataLabels: {
      enabled: false,
    },
    tooltip: {
      enabled: false,
    },
    legend: {
      show: true,
      position: "bottom",
      horizontalAlign: "left",
      fontFamily: "Outfit",
      fontSize: "14px",
      fontWeight: 400,
      markers: {
        size: 5,
        shape: "circle",
        radius: 999,
        strokeWidth: 0,
      },
      itemMargin: {
        horizontal: 10,
        vertical: 6,
      },
    },
    responsive: [
      {
        breakpoint: 640,
        options: {
          chart: {
            width: 380,
          },
          legend: {
            itemMargin: {
              horizontal: 7,
              vertical: 5,
            },
          },
        },
      },
      {
        breakpoint: 375,
        options: {
          legend: {
            fontSize: "12px",
          },
        },
      },
      {
        breakpoint: 1500,
        options: {
          legend: {
            itemMargin: {
              horizontal: 10,
              vertical: 6,
            },
          },
        },
      },
    ],
  };

  const chartSelector = document.querySelectorAll("#chartSixteen");

  if (chartSelector.length) {
    const chartSixteen = new ApexCharts(
      document.querySelector("#chartSixteen"),
      chartSixteenOptions,
    );
    chartSixteen.render();
  }
};

export default chart16;
