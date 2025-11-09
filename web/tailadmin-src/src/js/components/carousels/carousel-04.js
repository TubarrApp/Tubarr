// import Swiper JS
import Swiper from "swiper";
import { Navigation, Autoplay, Pagination } from "swiper/modules";
// import Swiper styles
import "swiper/css";
import "swiper/css/pagination";

const swiper = new Swiper(".carouselFour", {
  modules: [Navigation, Pagination, Autoplay],
  autoplay: {
    delay: 5000,
    disableOnInteraction: false,
  },
  pagination: {
    el: ".swiper-pagination",
    clickable: true,
  },
  navigation: {
    nextEl: ".swiper-button-next",
    prevEl: ".swiper-button-prev",
  },
});
