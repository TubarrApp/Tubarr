import ApexCharts from "apexcharts";

// ===== chartOne
const chart21 = () => {
  const churnOptions = {
    series: [
      {
        name: "Churn Rate",
        data: [4.5, 4.2, 4.6, 4.3, 4.1, 4.2, 4.26],
      },
    ],
    chart: {
      type: "area",
      height: 60,
      sparkline: {
        enabled: true,
      },
      animations: {
        enabled: true,
        easing: "easeinout",
        speed: 800,
      },
      toolbar: {
        show: false,
      },
    },
    colors: ["#ef4444"],
    stroke: {
      curve: "smooth",
      width: 2,
    },
    fill: {
      type: "gradient",
      gradient: {
        shadeIntensity: 1,
        opacityFrom: 0.6,
        opacityTo: 0.1,
        stops: [0, 100],
      },
    },
    tooltip: {
      fixed: {
        enabled: false,
      },
      x: {
        show: false,
      },
      y: {
        formatter: function (value) {
          return value.toFixed(2) + "%";
        },
      },
      marker: {
        show: false,
      },
    },
  };

  const chartSelector = document.querySelectorAll("#chartTwentyOne");

  if (chartSelector.length) {
    const chartTwentyOne = new ApexCharts(
      document.querySelector("#chartTwentyOne"),
      churnOptions,
    );
    chartTwentyOne.render();
  }
};

export default chart21;
